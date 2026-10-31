package buste

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/audit"
)

// La modifica della busta (RIP-S-04…11), come Workshop.update_repair/4, kind_change/2,
// clear_expected_on/2 e Repair.update_changeset/2 dello standalone (go-baseline); i casi da
// workshop_test.exs, workshop/kind_change_test.exs e workshop/public_link_test.exs.

func p(s string) *string { return &s }
func pi(n int64) *int64  { return &n }
func pb(v bool) *bool    { return &v }

// bustaDaModificare crea la busta del setup di workshop_test.exs: «Ridurre anello», accettata il
// 05/09/2026, con il cliente «Ada Rossi» e il telefono «+39 333 1111111».
func bustaDaModificare(t *testing.T, b *Buste, f func(*Accettazione)) int64 {
	t.Helper()
	a := Accettazione{NomeCliente: "Ada Rossi", TelefonoCliente: "+39 333 1111111", DataAccettazione: "2026-09-05",
		Descrizione: "Ridurre anello", CodiceQR: "QR-MOD"}
	if f != nil {
		f(&a)
	}
	return crea(t, b, a).ID
}

func modifica(t *testing.T, b *Buste, id int64, m Modifica) Modificata {
	t.Helper()
	got, err := b.Modifica(ctx, id, m, nil, chi)
	if err != nil {
		t.Fatalf("modifica: %v", err)
	}
	return got
}

// colonne legge alcune colonne della busta, unite da «|», con «∅» per NULL.
func colonne(t *testing.T, b *Buste, id int64, cc ...string) string {
	t.Helper()
	parti := make([]string, len(cc))
	for i, c := range cc {
		parti[i] = "coalesce(CAST(" + c + " AS TEXT), '∅')"
	}
	return valore(t, b.d, `SELECT `+strings.Join(parti, " || '|' || ")+` FROM buste WHERE id = ?`, id).(string)
}

// fotoBusta è la riga intera della busta, con le righe d'audit e dei clienti: un rifiuto la lascia uguale.
func fotoBusta(t *testing.T, b *Buste, id int64) string {
	t.Helper()
	var s string
	if err := b.d.Lettura.QueryRow(`SELECT json_object('busta', (SELECT json_group_array(json_array(numero, nome_cliente, telefono_cliente,
		data_accettazione, data_prevista, data_prevista_testo, descrizione, note_pubbliche, note_interne, stato, compro_oro,
		preventivo_testo, preventivo_centesimi, prezzo_testo, prezzo_centesimi, giorni_in_piu, oreficeria_id, artigiano_id,
		codice_qr, token_pubblico, token_pubblico_revocato_il, messaggi_visibili_dal, ritardo_avvisato_il, aggiornato_il))
		FROM buste WHERE id = ?1), 'audit', (SELECT count(*) FROM eventi_audit), 'clienti',
		(SELECT group_concat(nome || ':' || telefono || ':' || aggiornato_il) FROM clienti),
		'versioni', (SELECT count(*) FROM versioni_stato_buste))`, id).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

// inventario: RIP-S-04
// Una modifica salva i campi del modulo (qui anche i centesimi, che solo il codice può scrivere)
// e niente altro: numero, cliente, stato, date di pronta e consegna e gettone restano; l'audit
// repair.updated ha l'attore e, senza cambi di relazione, cliente, giorni o scadenza tolta, i
// metadati vuoti.
func TestModificaCampi(t *testing.T) {
	b, d, _ := prova(t)
	id := bustaDaModificare(t, b, nil)
	if _, err := b.CambiaStato(ctx, id, "pronta", chi); err != nil {
		t.Fatal(err)
	}
	fisse := []string{"cliente_id", "numero", "stato", "data_pronta", "data_consegna", "hex(hash_token_pubblico)", "token_pubblico",
		"token_pubblico_revocato_il", "nome_cliente", "telefono_cliente", "compro_oro"}
	prima := colonne(t, b, id, fisse...)
	modifica(t, b, id, Modifica{Descrizione: p("Saldare\ncollana"), NotePubbliche: p("Nota pubblica"), NoteInterne: p("Nota privata"),
		DataAccettazione: p("2026-09-04"), DataPrevista: p("2026-09-30"), PreventivoCentesimi: pi(12345), PrezzoCentesimi: pi(0)})
	if got := colonne(t, b, id, "descrizione", "note_pubbliche", "note_interne", "data_accettazione", "data_prevista",
		"preventivo_centesimi", "prezzo_centesimi", "aggiornato_il"); got != "Saldare\ncollana|Nota pubblica|Nota privata|2026-09-04|2026-09-30|12345|0|"+istanteProva {
		t.Errorf("campi %q", got)
	}
	if got := colonne(t, b, id, fisse...); got != prima {
		t.Errorf("campi protetti cambiati:\n%s\n%s", prima, got)
	}
	if s := ultimoAudit(t, d, "repair.updated"); s != "repair.updated|repair|"+itoa(id)+"|ok|{}" {
		t.Errorf("audit %q", s)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.updated' AND attore = 'titolare@esempio.test'`); n != 1 {
		t.Errorf("righe repair.updated %d", n)
	}
}

// inventario: RIP-S-04
// Una modifica senza cambi non scrive la busta (aggiornato_il resta), ma l'audit sì, come
// repo.update di Ecto con un changeset vuoto e la riga del Multi.
func TestModificaSenzaCambi(t *testing.T) {
	b, d, o := prova(t)
	id := bustaDaModificare(t, b, func(a *Accettazione) { a.Preventivo = "40" })
	o.Sposta(3600e9)
	modifica(t, b, id, Modifica{Descrizione: p("Ridurre anello"), Preventivo: p("40"), NotePubbliche: p(""),
		DataAccettazione: p("2026-09-05"), GiorniInPiu: p("0"), CodiceQR: p("QR-MOD"), NomeCliente: p(" Ada Rossi "),
		TelefonoCliente: p("+39 333 1111111"), OreficeriaID: p(""), ArtigianoID: p(" ")})
	if got := colonne(t, b, id, "aggiornato_il", "preventivo_testo"); got != istanteProva+"|40" {
		t.Errorf("busta riscritta: %q", got)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.updated' AND dati = '{}'`); n != 1 {
		t.Errorf("repair.updated %d", n)
	}
}

// inventario: RIP-S-04
// Campi sbagliati, centesimi negativi e un audit che non si scrive lasciano la busta e l'audit
// come prima. Gli errori dei campi arrivano tutti insieme, con i testi dello standalone.
func TestModificaRifiutataNonScrive(t *testing.T) {
	b, _, _ := prova(t)
	id := bustaDaModificare(t, b, nil)
	prima := fotoBusta(t, b, id)
	for _, c := range []struct {
		m     Modifica
		campi Campi
	}{
		{Modifica{Descrizione: p("")}, Campi{"descrizione": {MsgObbligatorio}}},
		{Modifica{Descrizione: p("  \n ")}, Campi{"descrizione": {MsgObbligatorio}}},
		{Modifica{DataAccettazione: p("invalid")}, Campi{"data_accettazione": {MsgNonValido}}},
		{Modifica{DataAccettazione: p("")}, Campi{"data_accettazione": {MsgObbligatorio}}},
		{Modifica{DataAccettazione: p("2026-09-04T10:00:00")}, Campi{"data_accettazione": {MsgNonValido}}},
		{Modifica{DataPrevista: p("31/02/2026")}, Campi{"data_prevista": {MsgNonValido}}},
		{Modifica{PreventivoCentesimi: pi(-1)}, Campi{"preventivo_centesimi": {MsgCentesimi}}},
		{Modifica{PrezzoCentesimi: pi(-1)}, Campi{"prezzo_centesimi": {MsgCentesimi}}},
		{Modifica{GiorniInPiu: p("91")}, Campi{"giorni_in_piu": {MsgGiorniInPiu}}},
		{Modifica{GiorniInPiu: p("-1")}, Campi{"giorni_in_piu": {MsgGiorniInPiu}}},
		{Modifica{GiorniInPiu: p("99999999999999999999")}, Campi{"giorni_in_piu": {MsgGiorniInPiu}}},
		{Modifica{GiorniInPiu: p("3.5")}, Campi{"giorni_in_piu": {MsgNonValido}}},
		{Modifica{GiorniInPiu: p(" 3")}, Campi{"giorni_in_piu": {MsgNonValido}}},
		{Modifica{GiorniInPiu: p("")}, Campi{"giorni_in_piu": {MsgObbligatorio}}},
		{Modifica{NomeCliente: p("   ")}, Campi{"nome_cliente": {MsgObbligatorio}}},
		{Modifica{Descrizione: p(""), DataAccettazione: p("x"), Preventivo: p(strings.Repeat("9", 256))},
			Campi{"descrizione": {MsgObbligatorio}, "data_accettazione": {MsgNonValido}, "preventivo": {string(ErrImportoLungo)}}},
	} {
		_, err := b.Modifica(ctx, id, c.m, nil, chi)
		if got := erroriCampi(t, err); !reflect.DeepEqual(got, c.campi) {
			t.Errorf("%+v: %v, attesi %v", c.m, got, c.campi)
		}
	}
	// L'audit che non si scrive (attore vuoto) annulla la modifica.
	if _, err := b.Modifica(ctx, id, Modifica{Descrizione: p("New")}, nil, chi0()); err == nil {
		t.Error("modifica salvata senza audit")
	}
	if dopo := fotoBusta(t, b, id); dopo != prima {
		t.Errorf("il database è cambiato\n%s\n%s", prima, dopo)
	}
}

// inventario: RIP-S-04
// «Giorni in più» va da 0 a 90, con il segno ammesso come Integer.parse/1; l'audit dice da/a.
func TestModificaGiorniInPiu(t *testing.T) {
	b, d, _ := prova(t)
	id := bustaDaModificare(t, b, nil)
	for _, c := range []struct{ scritto, salvato string }{{"90", "90"}, {"+5", "5"}, {"0", "0"}, {"007", "7"}} {
		modifica(t, b, id, Modifica{GiorniInPiu: p(c.scritto)})
		if got := colonne(t, b, id, "giorni_in_piu"); got != c.salvato {
			t.Errorf("%q: %q", c.scritto, got)
		}
	}
	if s := ultimoAudit(t, d, "repair.updated"); s != "repair.updated|repair|"+itoa(id)+`|ok|{"extra_days":{"from":0,"to":7}}` {
		t.Errorf("audit %q", s)
	}
}

// inventario: RIP-S-04
// Una busta che non c'è, o nel cestino, non si modifica.
func TestModificaBustaNonTrovata(t *testing.T) {
	b, d, _ := prova(t)
	id := bustaDaModificare(t, b, nil)
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, id)
	for _, n := range []int64{id, 999} {
		if _, err := b.Modifica(ctx, n, Modifica{Descrizione: p("x")}, nil, chi); !errors.Is(err, ErrBustaNonTrovata) {
			t.Errorf("%d: %v", n, err)
		}
		if err := b.TogliData(ctx, n, chi); !errors.Is(err, ErrBustaNonTrovata) {
			t.Errorf("togli data %d: %v", n, err)
		}
		if _, err := b.Scheda(ctx, n); !errors.Is(err, ErrBustaNonTrovata) {
			t.Errorf("scheda %d: %v", n, err)
		}
	}
	if got := colonne(t, b, id, "descrizione"); got != "Ridurre anello" {
		t.Errorf("busta nel cestino modificata: %q", got)
	}
}

// inventario: RIP-S-05
// I prezzi tengono il testo scritto e hanno i centesimi solo se è un importo; svuotati tolgono
// testo e centesimi.
func TestModificaPrezziATesto(t *testing.T) {
	b, _, _ := prova(t)
	id := bustaDaModificare(t, b, func(a *Accettazione) { a.Preventivo = "400/500" })
	if got := colonne(t, b, id, "preventivo_testo", "preventivo_centesimi"); got != "400/500|∅" {
		t.Errorf("accettazione %q", got)
	}
	modifica(t, b, id, Modifica{Preventivo: p("120,50"), PrezzoFinale: p("da definire")})
	if got := colonne(t, b, id, "preventivo_testo", "preventivo_centesimi", "prezzo_testo", "prezzo_centesimi"); got != "120,50|12050|da definire|∅" {
		t.Errorf("prezzi %q", got)
	}
	modifica(t, b, id, Modifica{Preventivo: p(""), PrezzoFinale: p("  ")})
	if got := colonne(t, b, id, "preventivo_testo", "preventivo_centesimi", "prezzo_testo", "prezzo_centesimi"); got != "∅|∅|∅|∅" {
		t.Errorf("svuotati %q", got)
	}
	for _, v := range []string{"-1", "1.001", "da definire", "400/500"} {
		modifica(t, b, id, Modifica{Preventivo: p(v)})
		if got := colonne(t, b, id, "preventivo_testo", "preventivo_centesimi"); got != v+"|∅" {
			t.Errorf("%q: %q", v, got)
		}
	}
	_, err := b.Modifica(ctx, id, Modifica{PrezzoFinale: p(strings.Repeat("9", 256))}, nil, chi)
	if got := erroriCampi(t, err); !reflect.DeepEqual(got, Campi{"prezzo_finale": {string(ErrImportoLungo)}}) {
		t.Errorf("256 byte: %v", got)
	}
	modifica(t, b, id, Modifica{PrezzoFinale: p(strings.Repeat("9", 255))})
	if got := colonne(t, b, id, "length(prezzo_testo)", "prezzo_centesimi"); got != "255|∅" {
		t.Errorf("255 byte: %q", got)
	}
}

// inventario: RIP-S-05
// Il prezzo si confronta con quello che il modulo mostra (price_input/2): i centesimi senza testo
// si mostrano «123.45», e rimandati così non cambiano niente; «123,45» è un testo nuovo. La
// scrittura dei soli centesimi (solo dal codice) toglie il testo vecchio.
func TestModificaPrezzoComeMostrato(t *testing.T) {
	b, _, _ := prova(t)
	id := bustaDaModificare(t, b, nil)
	esegui(t, b.d, `UPDATE buste SET preventivo_centesimi = 12345, prezzo_centesimi = 0 WHERE id = ?`, id)
	modifica(t, b, id, Modifica{Preventivo: p("123.45"), PrezzoFinale: p("0.00")})
	if got := colonne(t, b, id, "preventivo_testo", "preventivo_centesimi", "prezzo_testo", "prezzo_centesimi"); got != "∅|12345|∅|0" {
		t.Errorf("rimandati %q", got)
	}
	modifica(t, b, id, Modifica{Preventivo: p("123,45")})
	if got := colonne(t, b, id, "preventivo_testo", "preventivo_centesimi"); got != "123,45|12345" {
		t.Errorf("testo nuovo %q", got)
	}
	altra := bustaDaModificare(t, b, func(a *Accettazione) { a.Preventivo = "400/500" })
	modifica(t, b, altra, Modifica{PreventivoCentesimi: pi(5000)})
	if got := colonne(t, b, altra, "preventivo_testo", "preventivo_centesimi"); got != "∅|5000" {
		t.Errorf("soli centesimi %q", got)
	}
	// Testo e centesimi insieme: vince il testo, con i suoi centesimi (cast_price/4).
	modifica(t, b, altra, Modifica{Preventivo: p("da definire"), PreventivoCentesimi: pi(7000)})
	if got := colonne(t, b, altra, "preventivo_testo", "preventivo_centesimi"); got != "da definire|∅" {
		t.Errorf("testo e centesimi %q", got)
	}
}

// inventario: RIP-S-06
// La consegna a testo (da import) si tiene com'è e dà la data solo se comincia con una data vera;
// oltre 255 byte è un errore; una modifica di un altro campo non la tocca; una data scritta toglie
// il testo; la data svuotata toglie anche un testo riconosciuto.
func TestModificaConsegna(t *testing.T) {
	b, _, _ := prova(t)
	for _, c := range []struct{ testo, data string }{
		{"12/05/2026", "2026-05-12"}, {"2026-05-12", "2026-05-12"}, {"12/05/2026 pomeriggio", "2026-05-12"},
		{"2026-05-12 mattina", "2026-05-12"}, {"31/02/2026", "∅"}, {"12/5/2026", "∅"}, {"fine mese", "∅"},
	} {
		id := bustaDaModificare(t, b, nil)
		modifica(t, b, id, Modifica{DataPrevistaTesto: p(c.testo)})
		if got := colonne(t, b, id, "data_prevista_testo", "data_prevista"); got != c.testo+"|"+c.data {
			t.Errorf("%q: %q", c.testo, got)
		}
	}
	id := bustaDaModificare(t, b, nil)
	_, err := b.Modifica(ctx, id, Modifica{DataPrevistaTesto: p(strings.Repeat("x", 256))}, nil, chi)
	if got := erroriCampi(t, err); !reflect.DeepEqual(got, Campi{"data_prevista_testo": {string(ErrConsegnaLunga)}}) {
		t.Errorf("256 byte: %v", got)
	}
	modifica(t, b, id, Modifica{DataPrevistaTesto: p(strings.Repeat("x", 255))})
	if got := colonne(t, b, id, "length(data_prevista_testo)"); got != "255" {
		t.Errorf("255 byte: %q", got)
	}

	id = bustaDaModificare(t, b, nil)
	modifica(t, b, id, Modifica{DataPrevistaTesto: p("fine mese")})
	// Il modulo rimanda la data vuota e il testo nascosto com'era: niente cambia.
	modifica(t, b, id, Modifica{Descrizione: p("Altro lavoro"), DataPrevista: p(""), DataPrevistaTesto: p("fine mese")})
	if got := colonne(t, b, id, "data_prevista_testo", "data_prevista", "descrizione"); got != "fine mese|∅|Altro lavoro" {
		t.Errorf("modifica ordinaria %q", got)
	}
	modifica(t, b, id, Modifica{DataPrevista: p("2026-10-01"), DataPrevistaTesto: p("fine mese")})
	if got := colonne(t, b, id, "data_prevista_testo", "data_prevista"); got != "∅|2026-10-01" {
		t.Errorf("data scritta %q", got)
	}

	id = bustaDaModificare(t, b, nil)
	modifica(t, b, id, Modifica{DataPrevistaTesto: p("12/05/2026")})
	modifica(t, b, id, Modifica{DataPrevista: p("")})
	if got := colonne(t, b, id, "data_prevista_testo", "data_prevista"); got != "∅|∅" {
		t.Errorf("data svuotata %q", got)
	}
}

// inventario: RIP-S-06
// La scadenza tolta va nell'audit con quella che era (il testo, se c'era); metterla o spostarla
// non scrive niente di nuovo. Un giorno promesso diverso riapre l'avviso di ritardo (slice 211):
// lo stesso giorno o nessun giorno lo lasciano, e l'audit lo dice solo se c'era.
func TestModificaConsegnaNellAudit(t *testing.T) {
	b, d, _ := prova(t)
	id := bustaDaModificare(t, b, func(a *Accettazione) { a.DataPrevista = "2026-09-20" })
	modifica(t, b, id, Modifica{DataPrevista: p("2026-09-25")})
	if s := ultimoAudit(t, d, "repair.updated"); !strings.HasSuffix(s, "|ok|{}") {
		t.Errorf("spostata %q", s)
	}
	modifica(t, b, id, Modifica{DataPrevista: p("")})
	if s := ultimoAudit(t, d, "repair.updated"); !strings.HasSuffix(s, `|ok|{"expected_on":{"from":"2026-09-25","to":null}}`) {
		t.Errorf("tolta %q", s)
	}
	modifica(t, b, id, Modifica{DataPrevistaTesto: p("fine mese")})
	modifica(t, b, id, Modifica{DataPrevistaTesto: p("")})
	if s := ultimoAudit(t, d, "repair.updated"); !strings.HasSuffix(s, `|ok|{"expected_on":{"from":"fine mese","to":null}}`) {
		t.Errorf("testo tolto %q", s)
	}

	esegui(t, d, `UPDATE buste SET data_prevista = '2026-09-20', ritardo_avvisato_il = ? WHERE id = ?`, istanteProva, id)
	modifica(t, b, id, Modifica{DataPrevista: p("2026-09-20"), Descrizione: p("Stesso giorno")})
	modifica(t, b, id, Modifica{DataPrevistaTesto: p("fine mese")})
	if got := colonne(t, b, id, "ritardo_avvisato_il"); got != istanteProva {
		t.Errorf("avviso tolto senza un giorno nuovo: %q", got)
	}
	modifica(t, b, id, Modifica{DataPrevistaTesto: p("01/10/2026 sera")})
	if got := colonne(t, b, id, "ritardo_avvisato_il"); got != "∅" {
		t.Errorf("avviso non riaperto: %q", got)
	}
	if s := ultimoAudit(t, d, "repair.updated"); !strings.HasSuffix(s, `|ok|{"overdue_reopened":true}`) {
		t.Errorf("riapertura %q", s)
	}
}

// inventario: RIP-S-06
// «Togli data» toglie data e testo in un colpo, con l'audit della modifica; un audit che non si
// scrive lascia la consegna.
func TestTogliData(t *testing.T) {
	b, d, _ := prova(t)
	id := bustaDaModificare(t, b, func(a *Accettazione) { a.DataPrevista = "2026-09-20" })
	if err := b.TogliData(ctx, id, chi0()); err == nil {
		t.Error("tolta senza audit")
	}
	if got := colonne(t, b, id, "data_prevista"); got != "2026-09-20" {
		t.Errorf("tolta senza audit: %q", got)
	}
	if err := b.TogliData(ctx, id, chi); err != nil {
		t.Fatal(err)
	}
	if got := colonne(t, b, id, "data_prevista_testo", "data_prevista"); got != "∅|∅" {
		t.Errorf("data %q", got)
	}
	if s := ultimoAudit(t, d, "repair.updated"); s != "repair.updated|repair|"+itoa(id)+`|ok|{"expected_on":{"from":"2026-09-20","to":null}}` {
		t.Errorf("audit %q", s)
	}
	esegui(t, d, `UPDATE buste SET data_prevista_testo = '12/05/2026 pomeriggio', data_prevista = '2026-05-12' WHERE id = ?`, id)
	if err := b.TogliData(ctx, id, chi); err != nil {
		t.Fatal(err)
	}
	if got := colonne(t, b, id, "data_prevista_testo", "data_prevista"); got != "∅|∅" {
		t.Errorf("testo %q", got)
	}
	if s := ultimoAudit(t, d, "repair.updated"); !strings.HasSuffix(s, `{"expected_on":{"from":"12/05/2026 pomeriggio","to":null}}`) {
		t.Errorf("audit del testo %q", s)
	}
}

// inventario: RIP-S-07
// Nome e telefono si correggono sulla copia della busta: il cliente e le sue altre buste non
// cambiano. Spazi ai bordi tolti, telefono svuotato = stringa vuota, l'audit dice solo quali
// campi.
func TestModificaCliente(t *testing.T) {
	b, d, _ := prova(t)
	id := bustaDaModificare(t, b, nil)
	altra := bustaDaModificare(t, b, nil)
	clienti := valore(t, d, `SELECT group_concat(id || nome || telefono || aggiornato_il) FROM clienti`)
	modifica(t, b, id, Modifica{NomeCliente: p("  Ada Rossi Bianchi "), TelefonoCliente: p(" 333 999 ")})
	if got := colonne(t, b, id, "nome_cliente", "telefono_cliente"); got != "Ada Rossi Bianchi|333 999" {
		t.Errorf("copia %q", got)
	}
	if s := ultimoAudit(t, d, "repair.updated"); !strings.HasSuffix(s, `|ok|{"customer_fields":["name","phone"]}`) {
		t.Errorf("audit %q", s)
	}
	modifica(t, b, id, Modifica{TelefonoCliente: p("  ")})
	if got := colonne(t, b, id, "telefono_cliente"); got != "" {
		t.Errorf("telefono svuotato %q", got)
	}
	if s := ultimoAudit(t, d, "repair.updated"); !strings.HasSuffix(s, `|ok|{"customer_fields":["phone"]}`) {
		t.Errorf("audit %q", s)
	}
	// Telefono già vuoto, mandato vuoto: nessun cambio.
	modifica(t, b, id, Modifica{TelefonoCliente: p("")})
	if s := ultimoAudit(t, d, "repair.updated"); !strings.HasSuffix(s, `|ok|{}`) {
		t.Errorf("audit %q", s)
	}
	if got := colonne(t, b, altra, "nome_cliente", "telefono_cliente"); got != "Ada Rossi|+39 333 1111111" {
		t.Errorf("altra busta %q", got)
	}
	if got := valore(t, d, `SELECT group_concat(id || nome || telefono || aggiornato_il) FROM clienti`); got != clienti {
		t.Errorf("cliente cambiato: %v", got)
	}
	_, err := b.Modifica(ctx, id, Modifica{NomeCliente: p(strings.Repeat("é", 161)), TelefonoCliente: p(strings.Repeat("3", 41))}, nil, chi)
	if got := erroriCampi(t, err); !reflect.DeepEqual(got, Campi{"nome_cliente": {MsgNomeLungo}, "telefono_cliente": {MsgTelefonoLungo}}) {
		t.Errorf("limiti %v", got)
	}
	modifica(t, b, id, Modifica{NomeCliente: p(strings.Repeat("é", 160)), TelefonoCliente: p(strings.Repeat("3", 40))})
}

// inventario: RIP-S-07
// Su una busta conto terzi il telefono del cliente finale non si cambia nemmeno forzando il
// server; vale la relazione con cui la modifica finisce: spostata a un'oreficeria nella stessa
// modifica tiene il telefono salvato, riportata al cliente diretto lo può correggere.
func TestModificaTelefonoBloccato(t *testing.T) {
	b, d, _ := prova(t)
	id := bustaDaModificare(t, b, func(a *Accettazione) { a.OreficeriaID = "2" })
	modifica(t, b, id, Modifica{TelefonoCliente: p("000"), NomeCliente: p("Ada Bianchi")})
	if got := colonne(t, b, id, "nome_cliente", "telefono_cliente"); got != "Ada Bianchi|+39 333 1111111" {
		t.Errorf("conto terzi %q", got)
	}
	if s := ultimoAudit(t, d, "repair.updated"); !strings.HasSuffix(s, `|ok|{"customer_fields":["name"]}`) {
		t.Errorf("audit %q", s)
	}
	_, err := b.Modifica(ctx, id, Modifica{TelefonoCliente: p(strings.Repeat("3", 41))}, nil, chi)
	if err != nil {
		t.Errorf("un telefono bloccato non si valida: %v", err)
	}
	modifica(t, b, id, Modifica{OreficeriaID: p(""), TelefonoCliente: p("111")})
	if got := colonne(t, b, id, "telefono_cliente", "oreficeria_id"); got != "111|∅" {
		t.Errorf("tornata diretta %q", got)
	}
	modifica(t, b, id, Modifica{OreficeriaID: p("2"), TelefonoCliente: p("222")})
	if got := colonne(t, b, id, "telefono_cliente", "oreficeria_id"); got != "111|2" {
		t.Errorf("spostata a un'oreficeria %q", got)
	}
}

// inventario: RIP-S-08
// Una busta che cambia controparte perde il link nella stessa transazione: token in chiaro tolto
// (l'impronta resta), revoca e inizio della conversazione pubblica adesso, audit
// repair.public_link_revoked con il solo motivo. Lo stesso da un'oreficeria a un'altra e di nuovo
// al cliente; la riga 1 (il negozio) e nessuna oreficeria sono la stessa controparte.
func TestModificaCambioOreficeria(t *testing.T) {
	b, d, _ := prova(t)
	esegui(t, d, `INSERT INTO oreficerie (id, nome) VALUES (3, 'Oreficeria Seconda')`)
	id := bustaDaModificare(t, b, nil)
	impronta := colonne(t, b, id, "hex(hash_token_pubblico)")
	modifica(t, b, id, Modifica{Descrizione: p("Anello nuovo")})
	if got := colonne(t, b, id, "token_pubblico IS NOT NULL", "token_pubblico_revocato_il", "messaggi_visibili_dal"); got != "1|∅|∅" {
		t.Errorf("stessa controparte %q", got)
	}
	revoche := func() int {
		return conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.public_link_revoked' AND dati = '{"reason":"counterparty_changed"}'`)
	}
	modifica(t, b, id, Modifica{OreficeriaID: p("2")})
	if got := colonne(t, b, id, "token_pubblico", "token_pubblico_revocato_il", "messaggi_visibili_dal", "oreficeria_id", "hex(hash_token_pubblico)"); got != "∅|"+istanteProva+"|"+istanteProva+"|2|"+impronta {
		t.Errorf("revoca %q", got)
	}
	if n := revoche(); n != 1 {
		t.Errorf("revoche %d", n)
	}
	if s := ultimoAudit(t, d, "repair.updated"); !strings.HasSuffix(s, `|ok|{"oreficeria_id_from":null,"oreficeria_id_to":2}`) {
		t.Errorf("audit %q", s)
	}
	// Un link nuovo (come ensure_public_link/2), poi un'altra oreficeria e di nuovo il cliente.
	for i, o := range []string{"3", ""} {
		esegui(t, d, `UPDATE buste SET token_pubblico = 'nuovo', token_pubblico_revocato_il = NULL, messaggi_visibili_dal = NULL WHERE id = ?`, id)
		modifica(t, b, id, Modifica{OreficeriaID: p(o)})
		if got := colonne(t, b, id, "token_pubblico", "token_pubblico_revocato_il IS NOT NULL", "messaggi_visibili_dal IS NOT NULL"); got != "∅|1|1" {
			t.Errorf("verso %q: %q", o, got)
		}
		if n := revoche(); n != i+2 {
			t.Errorf("revoche %d", n)
		}
	}
	// La riga 1 e nessuna oreficeria: stessa controparte, il link resta.
	esegui(t, d, `UPDATE buste SET oreficeria_id = 1, token_pubblico = 'ancora', token_pubblico_revocato_il = NULL WHERE id = ?`, id)
	modifica(t, b, id, Modifica{OreficeriaID: p("")})
	if got := colonne(t, b, id, "token_pubblico", "oreficeria_id"); got != "ancora|∅" {
		t.Errorf("riga 1 → nessuna %q", got)
	}
	if s := ultimoAudit(t, d, "repair.updated"); !strings.HasSuffix(s, `|ok|{"oreficeria_id_from":1,"oreficeria_id_to":null}`) {
		t.Errorf("audit %q", s)
	}
	if n := revoche(); n != 3 {
		t.Errorf("revoche %d", n)
	}
	// Il token non finisce mai nell'audit.
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE dati LIKE '%nuovo%' OR dati LIKE '%ancora%'`); n != 0 {
		t.Errorf("token nell'audit: %d righe", n)
	}
}

// inventario: RIP-S-08
// Un link già revocato non si revoca due volte (niente audit), ma la conversazione pubblica
// riparte lo stesso da adesso.
func TestModificaOreficeriaLinkGiaRevocato(t *testing.T) {
	b, d, o := prova(t)
	id := bustaDaModificare(t, b, nil)
	esegui(t, d, `UPDATE buste SET token_pubblico = NULL, token_pubblico_revocato_il = '2026-10-01T08:00:00.000000Z' WHERE id = ?`, id)
	o.Sposta(60e9)
	modifica(t, b, id, Modifica{OreficeriaID: p("2")})
	if got := colonne(t, b, id, "token_pubblico_revocato_il", "messaggi_visibili_dal"); got != "2026-10-01T08:00:00.000000Z|2026-10-31T09:01:00.000000Z" {
		t.Errorf("già revocato %q", got)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.public_link_revoked'`); n != 0 {
		t.Errorf("revoche %d", n)
	}
}

// inventario: RIP-S-08
// L'oreficeria 1 (il negozio) forzata e un id che non c'è sono rifiutati senza toccare niente;
// spazi e vuoto sono «nessuna».
func TestModificaOreficeriaRifiutata(t *testing.T) {
	b, _, _ := prova(t)
	id := bustaDaModificare(t, b, nil)
	prima := fotoBusta(t, b, id)
	for _, v := range []string{"1", "999999", "0", "-2", "abc", "2x", "99999999999999999999"} {
		if _, err := b.Modifica(ctx, id, Modifica{OreficeriaID: p(v), Descrizione: p("cambiata")}, nil, chi); !errors.Is(err, ErrOreficeriaNonValida) {
			t.Errorf("%q: %v", v, err)
		}
	}
	if dopo := fotoBusta(t, b, id); dopo != prima {
		t.Errorf("il database è cambiato\n%s\n%s", prima, dopo)
	}
	modifica(t, b, id, Modifica{OreficeriaID: p(" 2 ")})
	if got := colonne(t, b, id, "oreficeria_id"); got != "2" {
		t.Errorf("con gli spazi %q", got)
	}
}

// inventario: RIP-S-09
// L'artigiano si assegna, si cambia e si toglie («— Nessuno —»), con da/a nell'audit; un id falso
// è «Seleziona un artigiano valido.» senza toccare la busta.
func TestModificaArtigiano(t *testing.T) {
	b, d, _ := prova(t)
	esegui(t, d, `INSERT INTO artigiani (id, nome) VALUES (2, 'Incastonatore Esempio')`)
	id := bustaDaModificare(t, b, nil)
	for _, c := range []struct{ scritto, salvato, audit string }{
		{"1", "1", `{"artisan_id_from":null,"artisan_id_to":1}`},
		{"2", "2", `{"artisan_id_from":1,"artisan_id_to":2}`},
		{"", "∅", `{"artisan_id_from":2,"artisan_id_to":null}`},
	} {
		modifica(t, b, id, Modifica{ArtigianoID: p(c.scritto)})
		if got := colonne(t, b, id, "artigiano_id"); got != c.salvato {
			t.Errorf("%q: %q", c.scritto, got)
		}
		if s := ultimoAudit(t, d, "repair.updated"); !strings.HasSuffix(s, "|ok|"+c.audit) {
			t.Errorf("%q: audit %q", c.scritto, s)
		}
	}
	prima := fotoBusta(t, b, id)
	for _, v := range []string{"999999", "0", "x"} {
		if _, err := b.Modifica(ctx, id, Modifica{ArtigianoID: p(v), Descrizione: p("Anello")}, nil, chi); !errors.Is(err, ErrArtigiano) {
			t.Errorf("%q: %v", v, err)
		}
	}
	if dopo := fotoBusta(t, b, id); dopo != prima {
		t.Errorf("il database è cambiato\n%s\n%s", prima, dopo)
	}
}

// inventario: RIP-S-08, RIP-S-09
// L'ordine dei rifiuti è quello del Multi: busta, tipo, oreficeria, artigiano, campi.
func TestModificaOrdineDeiRifiuti(t *testing.T) {
	b, _, _ := prova(t)
	id := bustaDaModificare(t, b, nil)
	esegui(t, b.d, `UPDATE buste SET consegnata_artigiano_il = ? WHERE id = ?`, istanteProva, id)
	m := Modifica{OreficeriaID: p("999"), ArtigianoID: p("999"), Descrizione: p("")}
	if _, err := b.Modifica(ctx, id, m, pb(true), chi); !errors.Is(err, ErrTipoFuoriBottega) {
		t.Errorf("tipo: %v", err)
	}
	if _, err := b.Modifica(ctx, id, m, nil, chi); !errors.Is(err, ErrOreficeriaNonValida) {
		t.Errorf("oreficeria: %v", err)
	}
	m.OreficeriaID = nil
	if _, err := b.Modifica(ctx, id, m, nil, chi); !errors.Is(err, ErrArtigiano) {
		t.Errorf("artigiano: %v", err)
	}
	m.ArtigianoID = nil
	if _, err := b.Modifica(ctx, id, m, nil, chi); erroriCampi(t, err) == nil {
		t.Errorf("campi: %v", err)
	}
}

// inventario: RIP-S-10
// Il QR è facoltativo: si scrive, vuoto si toglie, e due buste possono avere lo stesso codice.
func TestModificaQR(t *testing.T) {
	b, _, _ := prova(t)
	id := bustaDaModificare(t, b, nil)
	altra := bustaDaModificare(t, b, nil)
	modifica(t, b, id, Modifica{CodiceQR: p("QR-EDIT-7")})
	modifica(t, b, altra, Modifica{CodiceQR: p("QR-EDIT-7")})
	if got := colonne(t, b, id, "codice_qr") + "," + colonne(t, b, altra, "codice_qr"); got != "QR-EDIT-7,QR-EDIT-7" {
		t.Errorf("QR %q", got)
	}
	modifica(t, b, id, Modifica{CodiceQR: p(" ")})
	if got := colonne(t, b, id, "codice_qr"); got != "∅" {
		t.Errorf("QR tolto %q", got)
	}
	modifica(t, b, id, Modifica{CodiceQR: p(" QR 8 ")})
	if got := colonne(t, b, id, "codice_qr"); got != " QR 8 " {
		t.Errorf("QR con gli spazi %q", got)
	}
}

// inventario: RIP-S-11
// kind_change/2: lo stesso tipo non fa niente; uno stato che esiste nel tipo nuovo resta
// («annullata»); uno stato aperto si riallinea all'iniziale del tipo nuovo; una busta chiusa e una
// riparazione fuori bottega che diventerebbe compro oro sono rifiutate.
func TestCambioDiTipo(t *testing.T) {
	for _, c := range []struct {
		stato      string
		oro, fuori bool
		verso      bool
		atteso     CambioTipo
	}{
		{"in_lavorazione", false, false, false, CambioTipo{Esito: TipoUguale}},
		{"in_valutazione", true, false, true, CambioTipo{Esito: TipoUguale}},
		{"in_lavorazione", false, false, true, CambioTipo{Esito: TipoRiallinea, Da: "in_lavorazione", A: "in_valutazione"}},
		{"in_valutazione", true, false, false, CambioTipo{Esito: TipoRiallinea, Da: "in_valutazione", A: "in_lavorazione"}},
		{"preventivo", false, false, true, CambioTipo{Esito: TipoRiallinea, Da: "preventivo", A: "in_valutazione"}},
		{"in_attesa_materiali", false, false, true, CambioTipo{Esito: TipoRiallinea, Da: "in_attesa_materiali", A: "in_valutazione"}},
		{"pronta", false, false, true, CambioTipo{Esito: TipoRiallinea, Da: "pronta", A: "in_valutazione"}},
		{"valutata", true, false, false, CambioTipo{Esito: TipoRiallinea, Da: "valutata", A: "in_lavorazione"}},
		{"perfezionata", true, false, false, CambioTipo{Esito: TipoRiallinea, Da: "perfezionata", A: "in_lavorazione"}},
		{"annullata", false, false, true, CambioTipo{Esito: TipoResta, Da: "annullata", A: "annullata"}},
		{"annullata", true, false, false, CambioTipo{Esito: TipoResta, Da: "annullata", A: "annullata"}},
		{"consegnata", false, false, true, CambioTipo{Esito: TipoRifiutato, Rifiuto: ErrTipoChiusa}},
		{"chiusa", true, false, false, CambioTipo{Esito: TipoRifiutato, Rifiuto: ErrTipoChiusa}},
		{"in_lavorazione", false, true, true, CambioTipo{Esito: TipoRifiutato, Rifiuto: ErrTipoFuoriBottega}},
		// Fuori bottega e chiusa: vince fuori bottega (il primo controllo).
		{"consegnata", false, true, true, CambioTipo{Esito: TipoRifiutato, Rifiuto: ErrTipoFuoriBottega}},
		// Un compro oro fuori bottega torna riparazione: il controllo vale solo verso il compro oro.
		{"valutata", true, true, false, CambioTipo{Esito: TipoRiallinea, Da: "valutata", A: "in_lavorazione"}},
	} {
		s := Scheda{Stato: c.stato, CompraOro: c.oro, FuoriBottega: c.fuori}
		if got := CambioDiTipo(s, c.verso); got != c.atteso {
			t.Errorf("%s oro=%v fuori=%v → %v: %+v, atteso %+v", c.stato, c.oro, c.fuori, c.verso, got, c.atteso)
		}
	}
}

// inventario: RIP-S-11
// Il cambio di tipo nella modifica: solo tipo e stato si muovono (date, prezzi, avvisi, risposte,
// artigiano restano), con repair.kind_changed prima di repair.updated; il contatore degli stati
// conta il riallineamento; e all'indietro. «annullata» resta e il cambio si registra lo stesso.
func TestModificaCambioTipo(t *testing.T) {
	b, d, _ := prova(t)
	id := bustaDaModificare(t, b, func(a *Accettazione) { a.Preventivo = "12"; a.ArtigianoID = "1" })
	esegui(t, d, `UPDATE buste SET stato = 'pronta', data_pronta = '2026-09-10', avviso_pronta_il = ?1, preventivo_accettato_il = ?1 WHERE id = ?2`, istanteProva, id)
	fisse := []string{"data_pronta", "preventivo_centesimi", "avviso_pronta_il", "preventivo_accettato_il", "artigiano_id", "numero", "cliente_id"}
	prima := colonne(t, b, id, fisse...)
	got, err := b.Modifica(ctx, id, Modifica{}, pb(true), chi)
	if err != nil {
		t.Fatal(err)
	}
	if got != (Modificata{TipoCambiato: true, StatoPrima: "pronta", StatoDopo: "in_valutazione"}) {
		t.Errorf("esito %+v", got)
	}
	if got := colonne(t, b, id, "compro_oro", "stato"); got != "1|in_valutazione" {
		t.Errorf("tipo %q", got)
	}
	if dopo := colonne(t, b, id, fisse...); dopo != prima {
		t.Errorf("campi mossi:\n%s\n%s", prima, dopo)
	}
	if s := ultimoAudit(t, d, "repair.kind_changed"); s != "repair.kind_changed|repair|"+itoa(id)+`|ok|{"compro_oro":true,"status_from":"pronta","status_to":"in_valutazione"}` {
		t.Errorf("audit %q", s)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.updated'`); n != 1 {
		t.Errorf("repair.updated %d", n)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE id < (SELECT max(id) FROM eventi_audit WHERE azione = 'repair.kind_changed') AND azione = 'repair.updated'`); n != 0 {
		t.Error("repair.updated prima di repair.kind_changed")
	}
	// La preparazione ha già cambiato lo stato una volta (il trigger conta anche quella).
	if v, _ := b.Versione(ctx, id); v != 2 {
		t.Errorf("versione %d", v)
	}
	if got, _ := b.Modifica(ctx, id, Modifica{}, pb(false), chi); got != (Modificata{TipoCambiato: true, StatoPrima: "in_valutazione", StatoDopo: "in_lavorazione"}) {
		t.Errorf("all'indietro %+v", got)
	}
	if got := colonne(t, b, id, "compro_oro", "stato"); got != "0|in_lavorazione" {
		t.Errorf("all'indietro %q", got)
	}

	annullata := bustaDaModificare(t, b, nil)
	esegui(t, d, `UPDATE buste SET stato = 'annullata' WHERE id = ?`, annullata)
	if got, _ := b.Modifica(ctx, annullata, Modifica{}, pb(true), chi); got != (Modificata{TipoCambiato: true, StatoPrima: "annullata", StatoDopo: "annullata"}) {
		t.Errorf("annullata %+v", got)
	}
	if got := colonne(t, b, annullata, "compro_oro", "stato"); got != "1|annullata" {
		t.Errorf("annullata %q", got)
	}
	if s := ultimoAudit(t, d, "repair.kind_changed"); !strings.HasSuffix(s, `{"compro_oro":true,"status_from":"annullata","status_to":"annullata"}`) {
		t.Errorf("audit %q", s)
	}
	if v, _ := b.Versione(ctx, annullata); v != 1 {
		t.Errorf("annullata: versione %d", v)
	}
}

// inventario: RIP-S-11
// Lo stesso tipo, o nessun gesto, non scrive niente sul tipo; la riga salvata decide, non la
// pagina: chiedere di nuovo il compro oro a una busta che lo è già non fa niente.
func TestModificaTipoUguale(t *testing.T) {
	b, d, _ := prova(t)
	id := bustaDaModificare(t, b, nil)
	for _, oro := range []*bool{pb(false), nil} {
		if got, err := b.Modifica(ctx, id, Modifica{Descrizione: p("Anello bis")}, oro, chi); err != nil || got.TipoCambiato {
			t.Errorf("%v: %+v %v", oro, got, err)
		}
	}
	modifica(t, b, id, Modifica{})
	if _, err := b.Modifica(ctx, id, Modifica{}, pb(true), chi); err != nil {
		t.Fatal(err)
	}
	if got, err := b.Modifica(ctx, id, Modifica{}, pb(true), chi); err != nil || got.TipoCambiato {
		t.Errorf("di nuovo: %+v %v", got, err)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.kind_changed'`); n != 1 {
		t.Errorf("kind_changed %d", n)
	}
}

// inventario: RIP-S-11
// I rifiuti non scrivono niente: busta chiusa, riparazione fuori bottega; e un campo sbagliato
// nella stessa modifica si porta via il cambio di tipo.
func TestModificaTipoRifiutato(t *testing.T) {
	b, d, _ := prova(t)
	consegnata := bustaDaModificare(t, b, nil)
	esegui(t, d, `UPDATE buste SET stato = 'consegnata' WHERE id = ?`, consegnata)
	fuori := bustaDaModificare(t, b, nil)
	esegui(t, d, `UPDATE buste SET consegnata_artigiano_il = ? WHERE id = ?`, istanteProva, fuori)
	rientrata := bustaDaModificare(t, b, nil)
	esegui(t, d, `UPDATE buste SET consegnata_artigiano_il = ?1, rientrata_artigiano_il = ?1 WHERE id = ?2`, istanteProva, rientrata)
	for _, c := range []struct {
		id     int64
		m      Modifica
		atteso error
	}{
		{consegnata, Modifica{Descrizione: p("x")}, ErrTipoChiusa},
		{fuori, Modifica{}, ErrTipoFuoriBottega},
		{rientrata, Modifica{NomeCliente: p("  ")}, nil},
	} {
		prima := fotoBusta(t, b, c.id)
		_, err := b.Modifica(ctx, c.id, c.m, pb(true), chi)
		if c.atteso != nil && !errors.Is(err, c.atteso) {
			t.Errorf("%d: %v", c.id, err)
		}
		if c.atteso == nil {
			_ = erroriCampi(t, err) // fallisce da sé se non sono errori sui campi
		}
		if dopo := fotoBusta(t, b, c.id); dopo != prima {
			t.Errorf("%d: il database è cambiato\n%s\n%s", c.id, prima, dopo)
		}
	}
	if _, err := b.Modifica(ctx, rientrata, Modifica{}, pb(true), chi); err != nil {
		t.Errorf("rientrata: %v", err)
	}
}

// chi0 è un contesto senza attore: la riga d'audit non si scrive.
func chi0() audit.Contesto { return audit.Contesto{} }

// inventario: RIP-S-07
// Nome e telefono come cast/3 con update_change/3 di Ecto (misurato con l'Ecto 3.14.2
// dell'immagine): il valore scritto uguale a quello salvato, spazi compresi, non è un cambio; uno
// diverso si ripulisce dagli spazi e cambia solo se ripulito è diverso dal salvato. Una copia
// importata con gli spazi rimandata com'è non si riscrive e l'audit non nomina i campi.
func TestModificaClienteComeEcto(t *testing.T) {
	b, d, o := prova(t)
	id := bustaDaModificare(t, b, nil)
	esegui(t, d, `UPDATE buste SET nome_cliente = 'Ada Rossi ', telefono_cliente = ' 333 1 ' WHERE id = ?`, id)
	o.Sposta(3600e9)
	modifica(t, b, id, Modifica{NomeCliente: p("Ada Rossi "), TelefonoCliente: p(" 333 1 ")})
	if got := colonne(t, b, id, "nome_cliente", "telefono_cliente", "aggiornato_il"); got != "Ada Rossi | 333 1 |"+istanteProva {
		t.Errorf("rimandati uguali: %q", got)
	}
	if s := ultimoAudit(t, d, "repair.updated"); !strings.HasSuffix(s, "|ok|{}") {
		t.Errorf("audit %q", s)
	}
	// Diversi dal salvato: si ripuliscono e cambiano.
	modifica(t, b, id, Modifica{NomeCliente: p(" Ada Rossi "), TelefonoCliente: p("333 1")})
	if got := colonne(t, b, id, "nome_cliente", "telefono_cliente"); got != "Ada Rossi|333 1" {
		t.Errorf("ripuliti: %q", got)
	}
	if s := ultimoAudit(t, d, "repair.updated"); !strings.HasSuffix(s, `|ok|{"customer_fields":["name","phone"]}`) {
		t.Errorf("audit %q", s)
	}
	// Diversi grezzi, uguali ripuliti: nessun cambio.
	modifica(t, b, id, Modifica{NomeCliente: p("  Ada Rossi"), TelefonoCliente: p(" 333 1")})
	if s := ultimoAudit(t, d, "repair.updated"); !strings.HasSuffix(s, "|ok|{}") {
		t.Errorf("uguali ripuliti: %q", s)
	}
	// Un telefono salvato di soli spazi rimandato uguale: per il cast è nil, quindi diventa "".
	esegui(t, d, `UPDATE buste SET telefono_cliente = '  ' WHERE id = ?`, id)
	modifica(t, b, id, Modifica{TelefonoCliente: p("  ")})
	if got := colonne(t, b, id, "telefono_cliente"); got != "" {
		t.Errorf("soli spazi: %q", got)
	}
}

// inventario: RIP-S-04
// Una modifica vera riscrive aggiornato_il con l'ora della modifica.
func TestModificaAggiornataIl(t *testing.T) {
	b, _, o := prova(t)
	id := bustaDaModificare(t, b, nil)
	o.Sposta(3600e9)
	modifica(t, b, id, Modifica{Descrizione: p("Cambiata")})
	if got := colonne(t, b, id, "aggiornato_il"); got != "2026-10-31T10:00:00.000000Z" {
		t.Errorf("aggiornato_il %q", got)
	}
}

// inventario: RIP-S-06
// «Togli data» su una consegna solo a testo (data NULL) toglie il testo.
func TestTogliDataSoloTesto(t *testing.T) {
	b, d, _ := prova(t)
	id := bustaDaModificare(t, b, nil)
	esegui(t, d, `UPDATE buste SET data_prevista_testo = 'fine mese', data_prevista = NULL WHERE id = ?`, id)
	if err := b.TogliData(ctx, id, chi); err != nil {
		t.Fatal(err)
	}
	if got := colonne(t, b, id, "data_prevista_testo", "data_prevista"); got != "∅|∅" {
		t.Errorf("consegna %q", got)
	}
	if s := ultimoAudit(t, d, "repair.updated"); !strings.HasSuffix(s, `{"expected_on":{"from":"fine mese","to":null}}`) {
		t.Errorf("audit %q", s)
	}
}

// inventario: RIP-S-11
// Se la riga repair.kind_changed non si scrive, il cambio di tipo e il resto della modifica non
// restano.
func TestModificaTipoAuditNonScritto(t *testing.T) {
	b, d, _ := prova(t)
	id := bustaDaModificare(t, b, nil)
	esegui(t, d, `CREATE TEMP TRIGGER no_tipo BEFORE INSERT ON eventi_audit WHEN NEW.azione = 'repair.kind_changed' BEGIN SELECT RAISE(ABORT, 'no'); END`)
	prima := fotoBusta(t, b, id)
	if _, err := b.Modifica(ctx, id, Modifica{Descrizione: p("Cambiata")}, pb(true), chi); err == nil {
		t.Error("cambio di tipo senza audit")
	}
	if dopo := fotoBusta(t, b, id); dopo != prima {
		t.Errorf("il database è cambiato\n%s\n%s", prima, dopo)
	}
}
