package buste

import (
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"
)

// Il popup «📲 Avvisa cliente» (CLI-29…36, RIP-S-23), come CustomerNotice, Outgoing (compose/3,
// resolve_link/3, record_handoff/3), ReadyNotice e Workshop.ensure_public_link/2 dello standalone
// (go-baseline): i casi di customer_notice_test.exs, outgoing_handoff_test.exs e
// ready_notice_test.exs. Dati inventati; il negozio di prova è quello di prova().

const baseProva = "https://negozio.esempio.test"

// bustaAvviso crea una busta diretta (o dell'oreficeria 2 con b2b) e scrive le colonne in più.
func bustaAvviso(t *testing.T, b *Buste, b2b bool, colonne string, arg ...any) int64 {
	t.Helper()
	c := crea(t, b, con(modulo(), func(a *Accettazione) {
		a.NomeCliente, a.TelefonoCliente, a.CodiceQR = "Ada Rossi", "333 111 2222", ""
		a.Descrizione = "Anello in oro giallo\nsecondo rigo"
		if b2b {
			a.OreficeriaID = "2"
		}
	}))
	if colonne != "" {
		esegui(t, b.d, `UPDATE buste SET `+colonne+` WHERE id = ?`, append(arg, c.ID)...)
	}
	return c.ID
}

func profiloProva(t *testing.T, b *Buste, citta, orari, instagram, facebook, sito any) {
	t.Helper()
	esegui(t, b.d, `INSERT INTO profilo_negozio (nome, citta, orari, instagram, facebook, sito, condizioni_garanzia, creato_il, aggiornato_il)
		VALUES ('Bottega Prova', ?, ?, ?, ?, ?, 'G', ?, ?)`, citta, orari, instagram, facebook, sito, istanteProva, istanteProva)
}

func apriAvviso(t *testing.T, b *Buste, id int64, scopo ScopoAvviso) BozzaAvviso {
	t.Helper()
	z, err := b.ApriAvviso(ctx, id, scopo)
	if err != nil {
		t.Fatal(err)
	}
	return z
}

// disegnata è quello che il popup manda di nuovo all'«Invia» (i campi nascosti).
func disegnata(z BozzaAvviso) Disegnata {
	return Disegnata{Scopo: z.Scopo, Tipo: z.Tipo, Canale: z.Canale, Indirizzo: z.Indirizzo, OreficeriaID: z.OreficeriaID}
}

// stato delle tabelle che l'avviso può toccare: aprire e un rifiuto non le cambiano.
func statoAvvisi(t *testing.T, b *Buste) string {
	t.Helper()
	var s string
	if err := b.d.Lettura.QueryRow(`SELECT (SELECT count(*) FROM eventi_audit) || '|' || (SELECT count(*) FROM messaggi_in_uscita) || '|' ||
		(SELECT count(*) FROM eventi_avviso_pronta) || '|' || (SELECT group_concat(id || ':' || coalesce(link_inviato_il, '-') || ':' ||
		coalesce(avviso_pronta_il, '-') || ':' || coalesce(token_pubblico, '-') || ':' || aggiornato_il, ',') FROM buste)`).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

// inventario: CLI-29, CLI-33, CLI-34
// Un cliente diretto senza Telegram: WhatsApp al suo numero per wa.me, il testo del primo invio
// (wa_link_stato) con nome, numero visibile (D-64), {link} letterale, negozio e città; aprire non
// scrive niente, nemmeno il link.
func TestApriAvvisoClienteDiretto(t *testing.T) {
	b, _, _ := prova(t)
	profiloProva(t, b, " Città Prova ", "Lun-Sab 9-19", nil, nil, nil)
	id := bustaAvviso(t, b, false, "")
	prima := statoAvvisi(t, b)
	z := apriAvviso(t, b, id, ScopoControparte)
	if statoAvvisi(t, b) != prima {
		t.Error("aprire ha scritto")
	}
	if z.Canale != "whatsapp" || z.Parte != ParteCliente || z.Tipo != TipoAggiornamento || z.Indirizzo != "+393331112222" ||
		z.Nome != "Ada Rossi" || z.Problema != "" || z.Modello != nil || !z.TelegramSpento || z.Numero != "N. 1/26" {
		t.Fatalf("bozza %+v", z)
	}
	atteso := "Buongiorno Ada Rossi, può seguire in tempo reale lo stato della Sua riparazione n° N. 1/26 da questo link:\n{link}\n\n👋 Bottega Prova 💍 Città Prova\nLun-Sab 9-19"
	if z.Testo != atteso {
		t.Errorf("testo:\n%q\natteso\n%q", z.Testo, atteso)
	}
	// Nemmeno su una busta senza un link vivo: aprire non emette il link.
	senzaLink := bustaAvviso(t, b, false, "token_pubblico = NULL")
	prima = statoAvvisi(t, b)
	apriAvviso(t, b, senzaLink, ScopoControparte)
	if statoAvvisi(t, b) != prima {
		t.Error("aprire ha emesso il link")
	}
	// Con un'etichetta il numero è quello dell'etichetta (D-64).
	esegui(t, b.d, `UPDATE buste SET codice_qr = 'RIP-2026-0045' WHERE id = ?`, id)
	if z := apriAvviso(t, b, id, ScopoControparte); z.Numero != "0045/26" || !strings.Contains(z.Testo, "n° 0045/26 da") {
		t.Errorf("con etichetta: %q %q", z.Numero, z.Testo)
	}
}

// inventario: CLI-31
// La scelta del modello: preventivo con o senza importo (dal preventivo, non dal prezzo), compro oro
// valutata, pronta (avviso di ritiro), primo invio o aggiornamento secondo link_inviato_il; dopo una
// risposta sempre l'aggiornamento, anche su una busta pronta (e niente avviso di ritiro); la busta di
// un'oreficeria sempre il testo dell'oreficeria.
func TestSceltaDelModello(t *testing.T) {
	b, _, _ := prova(t)
	casi := []struct {
		nome, colonne string
		scopo         ScopoAvviso
		b2b           bool
		tipo          TipoAvviso
		inizio        string
	}{
		{"preventivo con importo", "stato = 'preventivo', preventivo_testo = '70,00', prezzo_centesimi = 9000", ScopoControparte, false, TipoAggiornamento, "Buongiorno Ada Rossi, c'è un preventivo"},
		{"preventivo senza importo", "stato = 'preventivo', prezzo_centesimi = 9000", ScopoControparte, false, TipoAggiornamento, "Buongiorno Ada Rossi, c'è un preventivo"},
		{"pronta", "stato = 'pronta'", ScopoControparte, false, TipoPronta, "Buongiorno Ada Rossi, la Sua riparazione n° N. 1/26 è pronta per il ritiro! 🎉"},
		{"valutata", "compro_oro = 1, stato = 'valutata'", ScopoControparte, false, TipoPronta, "Buongiorno Ada Rossi, la Sua scheda compro oro n° N. 1/26 è pronta per la firma! 🎉"},
		{"primo invio", "", ScopoControparte, false, TipoAggiornamento, "Buongiorno Ada Rossi, può seguire in tempo reale"},
		{"aggiornamento", "link_inviato_il = '" + istanteProva + "'", ScopoControparte, false, TipoAggiornamento, "Buongiorno Ada Rossi, c'è stato un aggiornamento"},
		{"risposta su pronta", "stato = 'pronta'", ScopoRisposta, false, TipoRisposta, "Buongiorno Ada Rossi, c'è stato un aggiornamento"},
		{"risposta primo invio", "", ScopoRisposta, false, TipoRisposta, "Buongiorno Ada Rossi, c'è stato un aggiornamento"},
		{"oreficeria pronta", "stato = 'pronta'", ScopoControparte, true, TipoPronta, "Ciao Oreficeria Esempio, aggiornamento sulla lavorazione:"},
		{"oreficeria risposta", "", ScopoRisposta, true, TipoRisposta, "Ciao Oreficeria Esempio, aggiornamento"},
	}
	for _, c := range casi {
		id := bustaAvviso(t, b, c.b2b, c.colonne)
		z := apriAvviso(t, b, id, c.scopo)
		numero := "N. " + itoa(id) + "/26"
		inizio := strings.ReplaceAll(c.inizio, "N. 1/26", numero)
		if z.Tipo != c.tipo || !strings.HasPrefix(z.Testo, inizio) {
			t.Errorf("%s: tipo %s, testo %q", c.nome, z.Tipo, z.Testo)
		}
	}
	// Il preventivo con importo riempie {preventivo} e {descrizione} (la prima riga).
	esegui(t, b.d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('outgoing.template.wa_preventivo_con_importo', '[{preventivo}][{descrizione}]', ?)`, istanteProva)
	id := bustaAvviso(t, b, false, "stato = 'preventivo', preventivo_centesimi = 7050")
	if z := apriAvviso(t, b, id, ScopoControparte); z.Testo != "[70,50][Anello in oro giallo]" {
		t.Errorf("preventivo: %q", z.Testo)
	}
}

// inventario: CLI-28, CLI-33
// Busta di un'oreficeria: destinatario l'oreficeria (numero e nome suoi), mai il cliente finale
// anche se la busta ha il suo telefono; il testo dell'oreficeria riga per riga: la prima riga della
// lavorazione, lo stato in maiuscolo, il prezzo finale prima del preventivo, la consegna, le note; le
// righe che mancano spariscono. Senza numero dell'oreficeria: «nessun recapito», mai il cliente.
func TestAvvisoOreficeria(t *testing.T) {
	b, _, _ := prova(t)
	id := bustaAvviso(t, b, true, "stato = 'in_attesa_materiali', note_pubbliche = ' da lucidare ', data_prevista_testo = 'fine mese', preventivo_centesimi = 7000, prezzo_testo = ' 80,00 '")
	z := apriAvviso(t, b, id, ScopoControparte)
	if z.Parte != ParteOreficeria || z.Indirizzo != "+390490000000" || z.Nome != "Oreficeria Esempio" || z.TelegramSpento {
		t.Fatalf("bozza %+v", z)
	}
	atteso := "Ciao Oreficeria Esempio, aggiornamento sulla lavorazione:\nCliente: Ada Rossi — busta n. N. " + itoa(id) + "/26\n" +
		"Lavorazione: Anello in oro giallo\nStato: IN ATTESA MATERIALI\nTotale: € 80,00\nConsegna prevista: fine mese\nNote: da lucidare\n" +
		"\nDettagli e foto: {link}\nNegozio"
	if z.Testo != atteso {
		t.Errorf("testo:\n%q\natteso\n%q", z.Testo, atteso)
	}
	nuda := bustaAvviso(t, b, true, "")
	z = apriAvviso(t, b, nuda, ScopoControparte)
	for _, riga := range []string{"Totale:", "Preventivo:", "Consegna prevista:", "Note:"} {
		if strings.Contains(z.Testo, riga) {
			t.Errorf("riga %q senza dato", riga)
		}
	}
	solo := bustaAvviso(t, b, true, "preventivo_centesimi = 7000, prezzo_testo = '   '")
	if z := apriAvviso(t, b, solo, ScopoControparte); !strings.Contains(z.Testo, "Preventivo: € 70,00\n") || strings.Contains(z.Testo, "Totale") {
		t.Errorf("solo preventivo: %q", z.Testo)
	}
	esegui(t, b.d, `UPDATE oreficerie SET telefono = '  ' WHERE id = 2`)
	z = apriAvviso(t, b, id, ScopoControparte)
	if z.Problema != ProblemaNessunRecapito || z.Testo != "" || z.Indirizzo != "" || z.Parte != ParteOreficeria {
		t.Errorf("oreficeria senza numero: %+v", z)
	}
	esegui(t, b.d, `UPDATE oreficerie SET telefono = 'nessuno' WHERE id = 2`)
	if z := apriAvviso(t, b, id, ScopoControparte); z.Problema != ProblemaNessunRecapito {
		t.Errorf("numero senza cifre: %+v", z)
	}
}

// inventario: CLI-29, CLI-34
// Un cliente senza numero, o con un numero senza cifre: il problema, nessun testo.
func TestAvvisoSenzaRecapito(t *testing.T) {
	b, _, _ := prova(t)
	for _, tel := range []string{"", "   ", "non ho il telefono"} {
		id := bustaAvviso(t, b, false, "telefono_cliente = ?", tel)
		z := apriAvviso(t, b, id, ScopoControparte)
		if z.Problema != ProblemaNessunRecapito || z.Testo != "" || z.Indirizzo != "" {
			t.Errorf("%q: %+v", tel, z)
		}
	}
	if _, err := b.ApriAvviso(ctx, 999, ScopoControparte); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("busta assente: %v", err)
	}
	id := bustaAvviso(t, b, false, "cancellata_il = ?", istanteProva)
	if _, err := b.ApriAvviso(ctx, id, ScopoControparte); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("nel cestino: %v", err)
	}
}

// inventario: CLI-33
// I segnaposto del negozio: senza profilo «Negozio» e niente città; {orari} e {social} dal profilo,
// senza spazi ai bordi, una riga chiusa per account nell'ordine Instagram, Facebook, Blog; vuoti se
// mancano. {stima} solo nei due testi del cliente con la frase e la riga vuota (qui spenta: niente).
func TestSegnapostoDelNegozio(t *testing.T) {
	b, _, _ := prova(t)
	esegui(t, b.d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('outgoing.template.wa_link_stato', '[{negozio}][{citta}][{orari}][{social}][{stima}][{nome}][{telegram_optin}]', ?)`, istanteProva)
	id := bustaAvviso(t, b, false, "nome_cliente = '  '")
	if z := apriAvviso(t, b, id, ScopoControparte); z.Testo != "[Negozio][][][][][Cliente][{telegram_optin}]" {
		t.Errorf("senza profilo: %q", z.Testo)
	}
	profiloProva(t, b, "Treviso", "   ", " @bottega ", "", "bottega.example")
	if z := apriAvviso(t, b, id, ScopoControparte); z.Testo != "[Bottega Prova][ 💍 Treviso][][Instagram: @bottega\nBlog: bottega.example\n][][Cliente][{telegram_optin}]" {
		t.Errorf("con profilo: %q", z.Testo)
	}
	esegui(t, b.d, `UPDATE profilo_negozio SET nome = '  '`)
	if z := apriAvviso(t, b, id, ScopoControparte); !strings.HasPrefix(z.Testo, "[Negozio]") {
		t.Errorf("nome vuoto: %q", z.Testo)
	}
}

// inventario: CLI-29
// Un testo salvato che non si compone non propone niente (nessun ripiego sul predefinito): la
// famiglia e l'avviso, il testo vuoto; il popup resta da scrivere a mano.
func TestAvvisoModelloNonValido(t *testing.T) {
	b, _, _ := prova(t)
	esegui(t, b.d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('outgoing.template.wa_link_stato', 'Ciao {password}', ?)`, istanteProva)
	id := bustaAvviso(t, b, false, "")
	z := apriAvviso(t, b, id, ScopoControparte)
	if z.Testo != "" || z.Modello == nil || z.Modello.Famiglia != FamLinkStato || z.Modello.Avviso != "Segnaposto non supportato: {password}" || z.Problema != "" {
		t.Fatalf("%+v", z)
	}
	if _, err := b.InviaAvviso(ctx, id, disegnata(z), "Scritto a mano", baseProva, chi); err != nil {
		t.Errorf("il testo scritto a mano parte: %v", err)
	}
}

// inventario: CLI-30, CLI-32, CLI-35
// «Invia» di un aggiornamento: il link della busta solo adesso (riusato se vivo, nessuna scrittura
// del link), una riga WhatsApp «accepted» con indirizzo, testo e istanti, il timbro del link inviato
// la prima volta, la riga outgoing.handed_off senza testo; l'indirizzo wa.me del testo registrato.
func TestInviaAggiornamento(t *testing.T) {
	b, _, _ := prova(t)
	id := bustaAvviso(t, b, false, "")
	token := tokenSalvato(t, b, id).(string)
	z := apriAvviso(t, b, id, ScopoControparte)
	testo := " \nCorretto a mano: {link} e {link}\n\n"
	inv, err := b.InviaAvviso(ctx, id, disegnata(z), testo, baseProva, chi)
	if err != nil {
		t.Fatal(err)
	}
	registrato := "Corretto a mano: " + baseProva + "/r/" + token + " e " + baseProva + "/r/" + token
	if inv.URL != IndirizzoWaMe("+393331112222", registrato) || inv.Tipo != TipoAggiornamento {
		t.Errorf("invio %+v", inv)
	}
	if n := quanteAzioni(t, b, "repair.public_link_issued"); n != 0 { // il link della busta nuova era vivo
		t.Errorf("link emessi %d", n)
	}
	riga := valore(t, b.d, `SELECT busta_id || '|' || canale || '|' || destinatario || '|' || indirizzo || '|' || coalesce(oggetto, '∅') || '|' || testo || '|' ||
		stato || '|' || tentativi || '|' || prossimo_tentativo_il || '|' || accodato_il || '|' || accettato_il || '|' || coalesce(fallito_il, '∅')
		FROM messaggi_in_uscita`)
	if riga != itoa(id)+"|whatsapp|customer|+393331112222|∅|"+registrato+"|accepted|1|"+istanteProva+"|"+istanteProva+"|"+istanteProva+"|∅" {
		t.Errorf("riga %v", riga)
	}
	mid := valore(t, b.d, `SELECT id FROM messaggi_in_uscita`).(int64)
	if a := ultimoAudit(t, b.d, "outgoing.handed_off"); a != "outgoing.handed_off|outgoing_message|"+itoa(mid)+`|ok|{"channel":"whatsapp","party":"customer","repair_id":`+itoa(id)+`}` {
		t.Errorf("audit %s", a)
	}
	if v := colonnaBusta(t, b, id, "link_inviato_il"); v != istanteProva {
		t.Errorf("timbro %v", v)
	}
	if conta(t, b.d, `SELECT count(*) FROM eventi_avviso_pronta`) != 0 {
		t.Error("un aggiornamento ha scritto un avviso di ritiro")
	}
	// Il timbro si mette una volta sola: il secondo invio non lo cambia.
	esegui(t, b.d, `UPDATE buste SET link_inviato_il = '2026-10-01T00:00:00.000000Z' WHERE id = ?`, id)
	z = apriAvviso(t, b, id, ScopoControparte)
	if _, err := b.InviaAvviso(ctx, id, disegnata(z), "Secondo", baseProva, chi); err != nil {
		t.Fatal(err)
	}
	if v := colonnaBusta(t, b, id, "link_inviato_il"); v != "2026-10-01T00:00:00.000000Z" {
		t.Errorf("timbro cambiato: %v", v)
	}
}

// inventario: CLI-32
// Un testo senza segnaposto non emette nessun link; un link che non vale più (solo impronta, scaduto,
// revocato) ne fa nascere uno nuovo con repair.public_link_issued e il motivo, e il vecchio smette di
// valere; {telegram_optin} è <base>/r/<token>/telegram, e su una busta dell'oreficeria è rifiutato
// senza scrivere niente. Senza indirizzo pubblico un testo con il link non parte.
func TestLinkDelMessaggio(t *testing.T) {
	b, _, _ := prova(t)
	id := bustaAvviso(t, b, false, "token_pubblico = NULL")
	z := apriAvviso(t, b, id, ScopoControparte)
	if _, err := b.InviaAvviso(ctx, id, disegnata(z), "Nessun link", baseProva, chi); err != nil {
		t.Fatal(err)
	}
	if tokenSalvato(t, b, id) != nil || quanteAzioni(t, b, "repair.public_link_issued") != 0 {
		t.Error("un testo senza link ha emesso il link")
	}
	for _, c := range []struct{ colonne, motivo string }{
		{"token_pubblico = NULL", "legacy_hash_only"},
		{"token_pubblico_scade_il = '2026-10-31T09:00:00.000000Z'", "expired"},
		{"token_pubblico_revocato_il = '2026-10-30T09:00:00.000000Z'", "revoked"},
	} {
		id := bustaAvviso(t, b, false, c.colonne)
		vecchio := colonnaBusta(t, b, id, "hash_token_pubblico")
		z := apriAvviso(t, b, id, ScopoControparte)
		inv, err := b.InviaAvviso(ctx, id, disegnata(z), "Qui {link} e {telegram_optin}", baseProva, chi)
		if err != nil {
			t.Fatal(err)
		}
		token, ok := tokenSalvato(t, b, id).(string)
		if !ok || !reToken.MatchString(token) {
			t.Fatalf("%s: token %v", c.motivo, tokenSalvato(t, b, id))
		}
		if string(colonnaBusta(t, b, id, "hash_token_pubblico").([]byte)) == string(vecchio.([]byte)) {
			t.Errorf("%s: impronta uguale", c.motivo)
		}
		if a := ultimoAudit(t, b.d, "repair.public_link_issued"); a != "repair.public_link_issued|repair|"+itoa(id)+`|ok|{"reason":"`+c.motivo+`"}` {
			t.Errorf("%s: audit %s", c.motivo, a)
		}
		testo := "Qui " + baseProva + "/r/" + token + " e " + baseProva + "/r/" + token + "/telegram"
		if inv.URL != IndirizzoWaMe("+393331112222", testo) {
			t.Errorf("%s: %q", c.motivo, inv.URL)
		}
		if colonnaBusta(t, b, id, "token_pubblico_revocato_il") != nil || colonnaBusta(t, b, id, "token_pubblico_scade_il") != "2027-01-29T09:00:00.000000Z" {
			t.Errorf("%s: revoca o scadenza", c.motivo)
		}
	}
	oref := bustaAvviso(t, b, true, "token_pubblico = NULL")
	z = apriAvviso(t, b, oref, ScopoControparte)
	prima := statoAvvisi(t, b)
	if _, err := b.InviaAvviso(ctx, oref, disegnata(z), "Iscriviti {telegram_optin} {link}", baseProva, chi); !errors.Is(err, ErrTelegramSoloCliente) {
		t.Errorf("telegram all'oreficeria: %v", err)
	}
	if statoAvvisi(t, b) != prima {
		t.Error("il rifiuto ha scritto")
	}
	if _, err := b.InviaAvviso(ctx, oref, disegnata(z), "Ecco {link}", "", chi); !errors.Is(err, ErrLinkNonSalvato) {
		t.Errorf("senza indirizzo pubblico: %v", err)
	}
	if statoAvvisi(t, b) != prima {
		t.Error("senza indirizzo pubblico ha scritto")
	}
}

// inventario: CLI-28, CLI-35
// Il messaggio all'oreficeria va al suo numero con destinatario «oreficeria» e non mette mai il
// timbro del link inviato (è del cliente); una busta pronta, consegnata o chiusa non lo prende.
func TestTimbroLinkInviato(t *testing.T) {
	b, _, _ := prova(t)
	oref := bustaAvviso(t, b, true, "")
	z := apriAvviso(t, b, oref, ScopoControparte)
	if _, err := b.InviaAvviso(ctx, oref, disegnata(z), "Ciao", baseProva, chi); err != nil {
		t.Fatal(err)
	}
	if colonnaBusta(t, b, oref, "link_inviato_il") != nil {
		t.Error("timbro su una busta dell'oreficeria")
	}
	if v := valore(t, b.d, `SELECT destinatario || indirizzo FROM messaggi_in_uscita WHERE busta_id = ?`, oref); v != "oreficeria+390490000000" {
		t.Errorf("destinatario %v", v)
	}
	for _, colonne := range []string{"stato = 'consegnata', data_consegna = '2026-10-31'", "compro_oro = 1, stato = 'chiusa'"} {
		id := bustaAvviso(t, b, false, colonne)
		z := apriAvviso(t, b, id, ScopoRisposta)
		if _, err := b.InviaAvviso(ctx, id, disegnata(z), "Ciao", baseProva, chi); err != nil {
			t.Fatal(err)
		}
		if colonnaBusta(t, b, id, "link_inviato_il") != nil {
			t.Errorf("%s: timbro", colonne)
		}
	}
}

// inventario: CLI-36, CLI-30
// L'avviso di ritiro: il blocco (avviso_pronta_il), la riga WhatsApp, l'evento «notified» con il
// messaggio e l'attore, outgoing.handed_off e ready_notice.sent; nessun timbro del link su una busta
// pronta. Un secondo avviso è rifiutato prima del link e non scrive niente; dopo, il popup torna
// l'aggiornamento.
func TestAvvisoDiRitiro(t *testing.T) {
	b, _, _ := prova(t)
	id := bustaAvviso(t, b, false, "stato = 'pronta', token_pubblico = NULL")
	z := apriAvviso(t, b, id, ScopoControparte)
	if z.Tipo != TipoPronta {
		t.Fatalf("tipo %s", z.Tipo)
	}
	if _, err := b.InviaAvviso(ctx, id, disegnata(z), z.Testo, baseProva, chi); err != nil {
		t.Fatal(err)
	}
	if v := colonnaBusta(t, b, id, "avviso_pronta_il"); v != istanteProva {
		t.Errorf("blocco %v", v)
	}
	if colonnaBusta(t, b, id, "link_inviato_il") != nil || colonnaBusta(t, b, id, "aggiornato_il") != istanteProva {
		t.Error("timbro del link o aggiornato_il")
	}
	mid := valore(t, b.d, `SELECT id FROM messaggi_in_uscita`).(int64)
	if e := valore(t, b.d, `SELECT busta_id || '|' || evento || '|' || canale || '|' || messaggio_in_uscita_id || '|' || attore || '|' || avvenuto_il FROM eventi_avviso_pronta`); e != itoa(id)+"|notified|whatsapp|"+itoa(mid)+"|titolare@esempio.test|"+istanteProva {
		t.Errorf("evento %v", e)
	}
	if a := ultimoAudit(t, b.d, "ready_notice.sent"); a != "ready_notice.sent|repair|"+itoa(id)+`|ok|{"channel":"whatsapp","outgoing_message_id":`+itoa(mid)+`,"party":"customer"}` {
		t.Errorf("audit %s", a)
	}
	if v := valore(t, b.d, `SELECT group_concat(azione, ',') FROM (SELECT azione FROM eventi_audit WHERE azione IN ('outgoing.handed_off', 'ready_notice.sent', 'repair.public_link_issued') ORDER BY id)`); v != "repair.public_link_issued,outgoing.handed_off,ready_notice.sent" {
		t.Errorf("ordine %v", v)
	}
	// Un popup disegnato prima, rimandato: rifiutato, senza link nuovo e senza righe.
	esegui(t, b.d, `UPDATE buste SET token_pubblico = NULL WHERE id = ?`, id)
	prima := statoAvvisi(t, b)
	if _, err := b.InviaAvviso(ctx, id, disegnata(z), z.Testo, baseProva, chi); !errors.Is(err, ErrGiaAvvisata) {
		t.Errorf("secondo avviso: %v", err)
	}
	if statoAvvisi(t, b) != prima {
		t.Error("il secondo avviso ha scritto")
	}
	// Dopo l'avviso il popup è un aggiornamento normale, con il testo dello stato (la pronta).
	if z := apriAvviso(t, b, id, ScopoControparte); z.Tipo != TipoAggiornamento || !strings.Contains(z.Testo, "pronta per il ritiro") {
		t.Errorf("dopo l'avviso: %s %q", z.Tipo, z.Testo)
	}
	// Il blocco è la scrittura stessa: anche chi ha passato i controlli (una gara persa) non avvisa
	// una busta già avvisata, e non scrive niente.
	prima = statoAvvisi(t, b)
	if err := b.avvisaRitiro(ctx, consegnaAMano{bustaID: id, parte: ParteCliente, indirizzo: "+393331112222", testo: "Gara"}, chi); !errors.Is(err, ErrGiaAvvisata) {
		t.Errorf("gara persa: %v", err)
	}
	if statoAvvisi(t, b) != prima {
		t.Error("la gara persa ha scritto")
	}
	// Una scheda compro oro valutata passa dallo stesso blocco.
	oro := bustaAvviso(t, b, false, "compro_oro = 1, stato = 'valutata'")
	z = apriAvviso(t, b, oro, ScopoControparte)
	if _, err := b.InviaAvviso(ctx, oro, disegnata(z), "Pronta", baseProva, chi); err != nil || colonnaBusta(t, b, oro, "avviso_pronta_il") == nil {
		t.Errorf("compro oro: %v", err)
	}
}

// inventario: CLI-36
// Una busta in un lotto ancora aperto non si avvisa da sola (lo fa il lotto); in un lotto già
// inviato sì.
func TestAvvisoInLotto(t *testing.T) {
	b, _, _ := prova(t)
	esegui(t, b.d, `INSERT INTO lotti_avviso (id, oreficeria_id, token, creato_il, aggiornato_il) VALUES (1, 2, 'lotto-uno', ?, ?)`, istanteProva, istanteProva)
	esegui(t, b.d, `INSERT INTO lotti_avviso (id, oreficeria_id, token, inviato_il, creato_il, aggiornato_il) VALUES (2, 2, 'lotto-due', ?, ?, ?)`, istanteProva, istanteProva, istanteProva)
	aperto := bustaAvviso(t, b, true, "stato = 'pronta', lotto_avviso_id = 1")
	z := apriAvviso(t, b, aperto, ScopoControparte)
	if z.Tipo != TipoAggiornamento {
		t.Errorf("in lotto aperto: %s", z.Tipo)
	}
	s, err := b.Scheda(ctx, aperto)
	if err != nil || s.Avviso.Stato != AvvisoInLottoAperto || s.Avviso.Lotto != 1 {
		t.Errorf("stato %+v %v", s.Avviso, err)
	}
	d := disegnata(z)
	d.Tipo = TipoPronta
	prima := statoAvvisi(t, b)
	if _, err := b.InviaAvviso(ctx, aperto, d, "Pronta", baseProva, chi); !errors.Is(err, ErrInLottoAperto) {
		t.Errorf("lotto aperto: %v", err)
	}
	if statoAvvisi(t, b) != prima {
		t.Error("ha scritto")
	}
	inviato := bustaAvviso(t, b, true, "stato = 'pronta', lotto_avviso_id = 2")
	z = apriAvviso(t, b, inviato, ScopoControparte)
	if z.Tipo != TipoPronta {
		t.Fatalf("lotto inviato: %s", z.Tipo)
	}
	if _, err := b.InviaAvviso(ctx, inviato, disegnata(z), "Pronta", baseProva, chi); err != nil {
		t.Errorf("lotto inviato: %v", err)
	}
}

// inventario: CLI-30, CLI-28
// L'«Invia» rilegge la busta e rifiuta senza scrivere se dal popup sono cambiati controparte, numero,
// canale, se un aggiornamento è diventato un avviso di ritiro o se la busta non è più pronta. Un testo
// vuoto (anche di soli spazi) è rifiutato.
func TestInviaRifiuti(t *testing.T) {
	b, _, _ := prova(t)
	id := bustaAvviso(t, b, false, "")
	z := apriAvviso(t, b, id, ScopoControparte)
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
	prova("vuoto", disegnata(z), " \n\t", ErrAvvisoVuoto)
	due := int64(2)
	d := disegnata(z)
	d.OreficeriaID = &due
	prova("controparte", d, "Ciao {link}", ErrControparteCambiata)
	uno := int64(1) // la riga 1 («il negozio») è la stessa controparte di nessuna oreficeria
	d.OreficeriaID = &uno
	if _, err := b.InviaAvviso(ctx, id, d, "Ciao", baseProva, chi); err != nil {
		t.Errorf("riga 1: %v", err)
	}
	d = disegnata(z)
	d.Indirizzo = "+390000000000"
	prova("numero", d, "Ciao", ErrRecapitoCambiato)
	d = disegnata(z)
	d.Canale = "telegram"
	prova("canale", d, "Ciao", ErrCanaleCambiato)
	esegui(t, b.d, `UPDATE buste SET telefono_cliente = '' WHERE id = ?`, id)
	prova("numero tolto", disegnata(z), "Ciao", ErrRecapitoCambiato)
	esegui(t, b.d, `UPDATE buste SET telefono_cliente = '333 111 2222', stato = 'pronta' WHERE id = ?`, id)
	prova("diventata pronta", disegnata(z), "Ciao", ErrTipoCambiato)
	zp := apriAvviso(t, b, id, ScopoControparte)
	esegui(t, b.d, `UPDATE buste SET stato = 'in_lavorazione' WHERE id = ?`, id)
	prova("non più pronta", disegnata(zp), "Ciao", ErrNonPronta)
	// La risposta su una busta pronta resta un messaggio normale: niente avviso, niente rifiuto.
	esegui(t, b.d, `UPDATE buste SET stato = 'pronta' WHERE id = ?`, id)
	zr := apriAvviso(t, b, id, ScopoRisposta)
	if _, err := b.InviaAvviso(ctx, id, disegnata(zr), "Risposto", baseProva, chi); err != nil || colonnaBusta(t, b, id, "avviso_pronta_il") != nil {
		t.Errorf("risposta su pronta: %v", err)
	}
	if _, err := b.InviaAvviso(ctx, 999, disegnata(z), "Ciao", baseProva, chi); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("busta assente: %v", err)
	}
}

// inventario: CLI-36, RIP-S-23
// «Azzera l'avviso»: toglie il blocco, aggiunge l'evento «reset» (lo storico resta) e la riga
// ready_notice.reset con lo stato e l'istante annullato; su una busta non avvisata è rifiutato senza
// scrivere. Dopo, la busta si avvisa di nuovo e lo storico ha i tre eventi in ordine.
func TestAzzeraAvviso(t *testing.T) {
	b, _, o := prova(t)
	id := bustaAvviso(t, b, false, "stato = 'pronta'")
	prima := statoAvvisi(t, b)
	if err := b.AzzeraAvviso(ctx, id, chi); !errors.Is(err, ErrNonAvvisata) {
		t.Errorf("non avvisata: %v", err)
	}
	if statoAvvisi(t, b) != prima {
		t.Error("ha scritto")
	}
	z := apriAvviso(t, b, id, ScopoControparte)
	if _, err := b.InviaAvviso(ctx, id, disegnata(z), "Pronta", baseProva, chi); err != nil {
		t.Fatal(err)
	}
	s, _ := b.Scheda(ctx, id)
	if s.Avviso.Stato != AvvisoFatto || s.Avviso.Il == nil || s.Avviso.Il.Format("2006-01-02T15:04:05.000000Z") != istanteProva {
		t.Errorf("stato dopo l'avviso %+v", s.Avviso)
	}
	o.Sposta(time.Hour)
	if err := b.AzzeraAvviso(ctx, id, chi); err != nil {
		t.Fatal(err)
	}
	if colonnaBusta(t, b, id, "avviso_pronta_il") != nil || colonnaBusta(t, b, id, "aggiornato_il") != "2026-10-31T10:00:00.000000Z" {
		t.Error("blocco non tolto")
	}
	if a := ultimoAudit(t, b.d, "ready_notice.reset"); a != "ready_notice.reset|repair|"+itoa(id)+`|ok|{"notified_at":"`+istanteProva+`","status":"pronta"}` {
		t.Errorf("audit %s", a)
	}
	z = apriAvviso(t, b, id, ScopoControparte)
	if z.Tipo != TipoPronta {
		t.Errorf("dopo l'azzeramento: %s", z.Tipo)
	}
	if _, err := b.InviaAvviso(ctx, id, disegnata(z), "Di nuovo", baseProva, chi); err != nil {
		t.Fatal(err)
	}
	s, _ = b.Scheda(ctx, id)
	var storia []string
	for _, e := range s.Avviso.Storia {
		storia = append(storia, e.Evento+"|"+e.Attore)
	}
	if strings.Join(storia, ",") != "notified|titolare@esempio.test,reset|titolare@esempio.test,notified|titolare@esempio.test" {
		t.Errorf("storia %v", storia)
	}
	if err := b.AzzeraAvviso(ctx, 999, chi); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("assente: %v", err)
	}
}

// inventario: RIP-S-23
// Lo stato dell'avviso nella scheda: niente su una busta che non è pronta; «da avvisare» su una
// pronta; avvisata con l'istante anche se poi non è più pronta (un cambio di stato non toglie il
// blocco, ma la busta non è più «pronta»: niente riquadro).
func TestStatoAvvisoDellaScheda(t *testing.T) {
	b, _, _ := prova(t)
	lav := bustaAvviso(t, b, false, "")
	if s, _ := b.Scheda(ctx, lav); s.Avviso.Stato != AvvisoNonApplicabile {
		t.Errorf("in lavorazione: %+v", s.Avviso)
	}
	pr := bustaAvviso(t, b, false, "stato = 'pronta'")
	if s, _ := b.Scheda(ctx, pr); s.Avviso.Stato != AvvisoDaFare {
		t.Errorf("pronta: %+v", s.Avviso)
	}
	cons := bustaAvviso(t, b, false, "stato = 'consegnata', avviso_pronta_il = ?", istanteProva)
	if s, _ := b.Scheda(ctx, cons); s.Avviso.Stato != AvvisoNonApplicabile {
		t.Errorf("consegnata avvisata: %+v", s.Avviso)
	}
}

// inventario: CLI-30
// Il link nel testo non va nell'indirizzo come segnaposto: «{link}» diventa l'indirizzo vero, che
// nel wa.me è codificato.
func TestIndirizzoDelMessaggio(t *testing.T) {
	b, _, _ := prova(t)
	id := bustaAvviso(t, b, false, "")
	z := apriAvviso(t, b, id, ScopoControparte)
	inv, err := b.InviaAvviso(ctx, id, disegnata(z), z.Testo, baseProva, chi)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(inv.URL)
	if err != nil || u.Host != "wa.me" || u.Path != "/+393331112222" || strings.Contains(u.Query().Get("text"), "{link}") ||
		!strings.Contains(u.Query().Get("text"), baseProva+"/r/") {
		t.Errorf("%q", inv.URL)
	}
}

// inventario: CLI-30
// Il tipo rimandato dal popup deve essere uno dei tre, e «reply» vale solo con lo scopo della risposta:
// altrimenti ErrTipoCambiato e niente si scrive.
func TestInviaTipoNonValido(t *testing.T) {
	b, _, _ := prova(t)
	id := bustaAvviso(t, b, false, "stato = 'pronta'")
	z := apriAvviso(t, b, id, ScopoControparte)
	prima := statoAvvisi(t, b)
	for _, d := range []Disegnata{
		{Scopo: ScopoControparte, Tipo: TipoRisposta, Canale: z.Canale, Indirizzo: z.Indirizzo},
		{Scopo: ScopoControparte, Tipo: "altro", Canale: z.Canale, Indirizzo: z.Indirizzo},
		{Scopo: ScopoRisposta, Tipo: "", Canale: z.Canale, Indirizzo: z.Indirizzo},
	} {
		if _, err := b.InviaAvviso(ctx, id, d, "Ciao", baseProva, chi); !errors.Is(err, ErrTipoCambiato) {
			t.Errorf("%+v: %v", d, err)
		}
	}
	if statoAvvisi(t, b) != prima {
		t.Error("ha scritto")
	}
	zr := apriAvviso(t, b, id, ScopoRisposta)
	if _, err := b.InviaAvviso(ctx, id, disegnata(zr), "Risposto", baseProva, chi); err != nil {
		t.Errorf("risposta: %v", err)
	}
}

// inventario: CLI-36
// Il blocco dell'avviso guarda il lotto anche nella scrittura: una busta in un lotto aperto non si
// avvisa nemmeno se chi scrive ha passato i controlli prima.
func TestBloccoConLottoAperto(t *testing.T) {
	b, _, _ := prova(t)
	esegui(t, b.d, `INSERT INTO lotti_avviso (id, oreficeria_id, token, creato_il, aggiornato_il) VALUES (1, 2, 'lotto-uno', ?, ?)`, istanteProva, istanteProva)
	id := bustaAvviso(t, b, true, "stato = 'pronta', lotto_avviso_id = 1")
	prima := statoAvvisi(t, b)
	if err := b.avvisaRitiro(ctx, consegnaAMano{bustaID: id, parte: ParteOreficeria, indirizzo: "+390490000000", testo: "Gara"}, chi); !errors.Is(err, ErrInLottoAperto) {
		t.Errorf("lotto aperto: %v", err)
	}
	if statoAvvisi(t, b) != prima {
		t.Error("ha scritto")
	}
}

// inventario: CLI-32, CLI-28
// Il link si emette nella transazione che rilegge la controparte: un testo composto per il cliente su
// una busta passata nel frattempo a un'oreficeria non emette niente.
func TestLinkControparteCambiata(t *testing.T) {
	b, _, _ := prova(t)
	id := bustaAvviso(t, b, true, "token_pubblico = NULL")
	prima := statoAvvisi(t, b)
	if _, err := b.garantisciLink(ctx, id, nil, chi); !errors.Is(err, ErrControparteCambiata) {
		t.Errorf("controparte: %v", err)
	}
	if statoAvvisi(t, b) != prima || tokenSalvato(t, b, id) != nil {
		t.Error("ha emesso il link")
	}
}

// inventario: CLI-33
// {oref_nome} di un'oreficeria con il nome di soli spazi è «Collega».
func TestOreficeriaSenzaNome(t *testing.T) {
	b, _, _ := prova(t)
	esegui(t, b.d, `UPDATE oreficerie SET nome = '  ' WHERE id = 2`)
	id := bustaAvviso(t, b, true, "")
	if z := apriAvviso(t, b, id, ScopoControparte); !strings.HasPrefix(z.Testo, "Ciao Collega, aggiornamento") {
		t.Errorf("%q", z.Testo)
	}
}

// inventario: CLI-33
// {stima} è la frase della stima con il punto e la riga vuota dopo (RepairEstimate.message_line/1),
// con la stima accesa e abbastanza buste pronte (qui 20 da 5 giorni: «5-6»).
func TestSegnapostoStima(t *testing.T) {
	b, _, _ := prova(t)
	esegui(t, b.d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('stima_tempi_attiva', '1', ?), ('outgoing.template.wa_link_stato', '[{stima}]', ?)`, istanteProva, istanteProva)
	for i := 0; i < 20; i++ {
		bustaAvviso(t, b, false, "stato = 'consegnata', data_accettazione = '2026-10-01', data_pronta = '2026-10-06', data_consegna = '2026-10-07'")
	}
	id := bustaAvviso(t, b, false, "")
	if z := apriAvviso(t, b, id, ScopoControparte); z.Testo != "[Pronta di solito in 5-6 giorni dal ritiro.\n\n]" {
		t.Errorf("%q", z.Testo)
	}
}
